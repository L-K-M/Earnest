package ch.lkmc.earnest;

import androidx.room.Dao;
import androidx.room.Insert;
import androidx.room.OnConflictStrategy;
import androidx.room.Query;

@Dao
public interface SyncDao {
    @Query("SELECT * FROM SyncRow WHERE id = 1") SyncRow load();
    @Insert(onConflict = OnConflictStrategy.REPLACE) void save(SyncRow row);
}
